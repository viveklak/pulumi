// *** WARNING: this file was generated by test. ***
// *** Do not edit by hand unless you're certain you know what you are doing! ***

using System;
using System.Collections.Generic;
using System.Collections.Immutable;
using System.Threading.Tasks;
using Pulumi.Serialization;

namespace Pulumi.Example
{
    public static class ArgFunction
    {
        public static Task<ArgFunctionResult> InvokeAsync(ArgFunctionArgs? args = null, InvokeOptions? options = null)
            => global::Pulumi.Deployment.Instance.InvokeAsync<ArgFunctionResult>("example::argFunction", args ?? new ArgFunctionArgs(), options.WithDefaults());

        public static Output<ArgFunctionResult> Invoke(ArgFunctionInvokeArgs? args = null, InvokeOptions? options = null)
            => global::Pulumi.Deployment.Instance.Invoke<ArgFunctionResult>("example::argFunction", args ?? new ArgFunctionInvokeArgs(), options.WithDefaults());
    }


    public sealed class ArgFunctionArgs : global::Pulumi.InvokeArgs
    {
        [Input("name")]
        public Pulumi.Random.RandomPet? Name { get; set; }

        public ArgFunctionArgs()
        {
        }
        public static new ArgFunctionArgs Empty => new ArgFunctionArgs();
    }

    public sealed class ArgFunctionInvokeArgs : global::Pulumi.InvokeArgs
    {
        [Input("name")]
        public Input<Pulumi.Random.RandomPet>? Name { get; set; }

        public ArgFunctionInvokeArgs()
        {
        }
        public static new ArgFunctionInvokeArgs Empty => new ArgFunctionInvokeArgs();
    }


    [OutputType]
    public sealed class ArgFunctionResult
    {
        public readonly int? Age;

        [OutputConstructor]
        private ArgFunctionResult(int? age)
        {
            Age = age;
        }
    }
}
